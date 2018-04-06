$(document).ready(function() {
    //animate burger menu button
    $('#navbar').on('hide.bs.collapse show.bs.collapse', function () {
        $('#nav-animated-icon').toggleClass('open');
    });
});

var hostname = location.protocol + '//' + location.host;

// Perform basic AJAX request.
function performRequest(URL, httpMethod, data, resultMethod) {
    $.ajax({
        url: URL,
        type: httpMethod,
        dataType: 'text',
        data: data,
        error: function(e) {
            console.log(e);
            notifyAlert("Could not connect to the server.", "danger");
        },
        success: function(e) {
            resultMethod(e);
        },
        timeout: 10000
    });
}

// Get HTML for a warning/error HTML message.
function setAlertWindow(type, msg, target) {
    performRequest(hostname + "/static/templates/alert.html", "GET", "", function(result) {
        var replaced = result.replace("{{type}}", type);
        replaced = replaced.replace("{{msg}}", msg);
        if (typeof target === 'string' || target instanceof String) {
            target = $(target);
        }
        target.hide().empty().append(replaced).fadeIn(400);
    });
}

// Create a notify alert.
function notifyAlert(msg, type) {
    var icon = "glyphicon glyphicon-ok";
    if (type === "warning" || type === "danger") {
        icon = "glyphicon glyphicon-remove"
    }

    $.notify({
        message: "<strong>" + msg + "</strong>",
        icon: icon
    },{
        type: type,
        delay: 4000,
        newest_on_top: true,
        mouse_over: "pause"
    });
}

// Toggle button enabled & spinner visibility.
function setButtonProcessing(element, enabled) {
    if (enabled === true) {
        element.find(".btn-label").css("display", "none");
        element.find(".btn-spinner").css("display", "inline-block");
        element.attr("disabled", true);
        return
    }
    element.find(".btn-label").css("display", "inline-block");
    element.find(".btn-spinner").css("display", "none");
    element.attr("disabled", false);
}
