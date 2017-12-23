$(document).ready(function() {
    //animate burger menu button
    $('#navbar').on('hide.bs.collapse show.bs.collapse', function () {
        $('#nav-animated-icon').toggleClass('open');
        console.log("t")
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
        },
        success: function(e) {
            resultMethod(e);
        }
    });
}

// Get HTML for a warning/error HTML message.
function setAlertWindow(type, msg, target) {
    performRequest(hostname + "/static/alert.html", "GET", "", function(result) {
        var replaced = result.replace("{{type}}", type);
        replaced = replaced.replace("{{msg}}", msg);
        $(target).hide().empty().append(replaced).fadeIn(400);
    });
}

// Toggle button enabled & spinner visibility.
function setButtonProcessing(element, enabled) {
    if (enabled === true) {
        element.attr("disabled", true);
        element.find(".btn-label").hide();
        element.find(".btn-spinner").show();
        return
    }
    element.find(".btn-label").show();
    element.find(".btn-spinner").hide();
    element.attr("disabled", false);
}
