$(document).ready(function() {
    setButtonProcessing($("#login-btn"), false);

    // login form submit
    $("#login-form").submit(function(e) {
        e.preventDefault();

        var data = $(this).serialize();

        setButtonProcessing($("#login-btn"), true);

        performRequest(hostname + "/login", "post", data, function(result) {
            result = result.trim();

            if (result === "unauthorised") {
                setAlertWindow("warning", "Incorrect email address or password.", "#error-window");
                $("#password-input").val("");
                setButtonProcessing($("#login-btn"), false);
            }
            else if (result === "error") {
                setAlertWindow("danger", "A server error occurred.", "#error-window");
                $("#password-input").val("");
                setButtonProcessing($("#login-btn"), false);
            }
            else {
                window.location = "/";
            }
        });
    });
});